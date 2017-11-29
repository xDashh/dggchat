package dggchat

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// A Session represents a connection to destinygg chat.
type Session struct {
	sync.RWMutex
	// If true, attempt to reconnect on error
	AttempToReconnect bool

	readOnly  bool
	loginKey  string
	listening chan bool
	wsURL     url.URL
	ws        *websocket.Conn
	handlers  handlers
	state     *state
	dialer    *websocket.Dialer
}

type messageOut struct {
	Data string `json:"data"`
}

type privateMessageOut struct {
	Nick string `json:"nick"`
	Data string `json:"data"`
}

type banOut struct {
	Nick     string        `json:"nick"`
	Reason   string        `json:"reason,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
	Banip    bool          `json:"banip,omitempty"`
}

type subOnly struct {
	SubOnly bool `json:"subonly"`
}

type pingOut struct {
	Timestamp int64 `json:"timestamp"`
}

// ErrAlreadyOpen is thrown when attempting to open a web socket connection
// on a websocket that is already open.
var ErrAlreadyOpen = errors.New("web socket is already open")

// ErrReadOnly is thrown when attempting to send messages using a read-only session.
var ErrReadOnly = errors.New("session is read-only")

var wsURL = url.URL{Scheme: "wss", Host: "www.destiny.gg", Path: "/ws"}

// SetURL changes the url that will be used when connecting to the socket server.
// This should be done before calling *session.Open()
func (s *Session) SetURL(u url.URL) {
	s.wsURL = u
}

// SetDialer changes the websocket dialer that will be used when connecting to the socket server.
func (s *Session) SetDialer(d websocket.Dialer) {
	s.dialer = &d
}

// Open opens a websocket connection to destinygg chat.
func (s *Session) Open() error {
	s.Lock()
	defer s.Unlock()

	if s.ws != nil {
		return ErrAlreadyOpen
	}

	header := http.Header{}

	if !s.readOnly {
		header.Add("Cookie", fmt.Sprintf("authtoken=%s", s.loginKey))
	}

	ws, _, err := s.dialer.Dial(s.wsURL.String(), header)
	if err != nil {
		return err
	}

	s.ws = ws
	s.listening = make(chan bool)

	go s.listen(s.ws, s.listening)

	return nil
}

// Close cleanly closes the connection and stops running listeners
func (s *Session) Close() error {
	if s.ws == nil {
		return nil
	}

	err := s.ws.Close()
	if err != nil {
		return err
	}

	s.ws = nil

	return nil
}

func (s *Session) listen(ws *websocket.Conn, listening <-chan bool) {
	for {
		_, message, err := s.ws.ReadMessage()
		if err != nil {
			if ws != s.ws {
				return
			}

			err := ws.Close()
			if err != nil {
				return
			}

			s.reconnect()
		}

		mslice := strings.SplitN(string(message[:]), " ", 2)
		if len(mslice) != 2 {
			continue
		}

		mType := mslice[0]
		mContent := strings.Join(mslice[1:], " ")

		switch mType {

		case "MSG":
			m, err := parseMessage(mContent)
			if s.handlers.msgHandler == nil || err != nil {
				continue
			}
			s.handlers.msgHandler(m, s)

		case "MUTE":
			mute, err := parseMute(mContent, s)
			if s.handlers.muteHandler == nil || err != nil {
				continue
			}
			s.handlers.muteHandler(mute, s)

		case "UNMUTE":
			mute, err := parseMute(mContent, s)
			if s.handlers.muteHandler == nil || err != nil {
				continue
			}
			s.handlers.unmuteHandler(mute, s)

		case "BAN":
			ban, err := parseBan(mContent, s)
			if s.handlers.banHandler == nil || err != nil {
				continue
			}
			s.handlers.banHandler(ban, s)

		case "UNBAN":
			ban, err := parseBan(mContent, s)
			if s.handlers.banHandler == nil || err != nil {
				continue
			}
			s.handlers.unbanHandler(ban, s)

		case "SUBONLY":
			//TODO
		case "BROADCAST":
			if s.handlers.broadcastHandler == nil {
				continue
			}

			b, err := parseBroadcast(mContent)
			if err != nil {
				continue
			}

			s.handlers.broadcastHandler(b, s)

		case "PRIVMSG":
			if s.handlers.pmHandler == nil {
				continue
			}

			pm, err := parsePrivateMessage(mContent)
			if err != nil {
				continue
			}

			u, found := s.GetUser(pm.User.Nick)
			if found {
				pm.User = u
			}

			s.handlers.pmHandler(pm, s)

		case "PRIVMSGSENT":
			//TODO confirms the successful sending of a PM(?)
		case "PING":
		case "PONG":
			if s.handlers.pingHandler == nil {
				continue
			}

			p, err := parsePing(mContent)
			if err != nil {
				continue
			}

			s.handlers.pingHandler(p, s)
		case "ERR":
			if s.handlers.errHandler == nil {
				continue
			}

			errMessage := parseErrorMessage(mContent)
			s.handlers.errHandler(errMessage, s)
		case "NAMES":
			n, err := parseNames(mContent)
			if err != nil {
				continue
			}

			s.state.users = n.Users
			s.state.connections = n.Connections
		case "JOIN":
			ra, err := parseRoomAction(mContent)
			if err != nil {
				continue
			}

			s.state.addUser(ra.User)

			if s.handlers.joinHandler != nil {
				s.handlers.joinHandler(ra, s)
			}
		case "QUIT":
			ra, err := parseRoomAction(mContent)
			if err != nil {
				continue
			}

			s.state.removeUser(ra.User.Nick)

			if s.handlers.quitHandler != nil {
				s.handlers.quitHandler(ra, s)
			}
		case "REFRESH":
			//TODO voluntary reconnect when server determines state should be refreshed
		}

		select {
		case <-listening:
			return
		default:
		}
	}
}

func (s *Session) reconnect() {
	if !s.AttempToReconnect {
		return
	}

	wait := 1
	for {
		err := s.Open()
		if err == nil || err == ErrAlreadyOpen {
			return
		}

		wait *= 2
		<-time.After(time.Duration(wait) * time.Second)

		if wait > 600 {
			wait = 600
		}
	}
}

// GetUser attempts to find the user in the chat room state.
// If the user is found, returns the user and true,
// otherwise false is returned as the second parameter.
func (s *Session) GetUser(name string) (User, bool) {
	s.RLock()
	defer s.RUnlock()

	for _, user := range s.state.users {
		if strings.EqualFold(name, user.Nick) {
			return user, true
		}
	}

	return User{}, false
}

func (s *Session) send(message interface{}, mType string) error {
	if s.readOnly {
		return ErrReadOnly
	}
	m, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return s.ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("%s %s", mType, m)))
}

// SendMessage sends the given string as a message to chat.
// Note: a return error of nil does not guarantee successful delivery.
// Monitor for error events to ensure the message was sent with no errors.
func (s *Session) SendMessage(message string) error {
	m := messageOut{Data: message}
	return s.send(m, "MSG")
}

// SendMute mutes the user with the given nick.
func (s *Session) SendMute(nick string) error {
	m := messageOut{Data: nick}
	return s.send(m, "MUTE")
}

// SendUnmute unmutes the user with the given nick.
func (s *Session) SendUnmute(nick string) error {
	m := messageOut{Data: nick}
	return s.send(m, "UNMUTE")
}

// SendBan bans the user with the given nick.
// Bans require a ban reason to be specified. TODO: ban duration is optional
func (s *Session) SendBan(nick string, reason string, duration time.Duration, banip bool) error {
	b := banOut{
		Nick:     nick,
		Reason:   reason,
		Duration: duration,
	}
	return s.send(b, "BAN")
}

// SendUnban unbans the user with the given nick.
// Unbanning also removes mutes.
func (s *Session) SendUnban(nick string) error {
	b := messageOut{Data: nick}
	return s.send(b, "UNBAN")
}

// SendAction calls the SendMessage method but also adds
// "/me" in front of the message to make it a chat action
// same caveat with the returned error value applies.
func (s *Session) SendAction(message string) error {
	return s.SendMessage(fmt.Sprintf("/me %s", message))
}

// SendPrivateMessage sends the given user a private message.
func (s *Session) SendPrivateMessage(nick string, message string) error {
	p := privateMessageOut{
		Nick: nick,
		Data: message,
	}
	return s.send(p, "PRIVMSG")
}

// SendSubOnly modifies the chat subonly mode.
// During subonly mode, only subscribers and some other special user classes are allowed to send messages.
func (s *Session) SendSubOnly(subonly bool) error {
	so := subOnly{SubOnly: subonly}
	return s.send(so, "SUBONLY")
}

// SendPing sends a ping to the server with the current timestamp.
func (s *Session) SendPing() error {
	t := pingOut{Timestamp: timeToUnix(time.Now())}
	return s.send(t, "PING")
}
