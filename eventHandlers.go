package dggchat

type handlers struct {
	msgHandler       func(Message, *Session)
	errHandler       func(string, *Session)
	joinHandler      func(RoomAction, *Session)
	quitHandler      func(RoomAction, *Session)
	pmHandler        func(PrivateMessage, *Session)
	broadcastHandler func(Broadcast, *Session)
	pingHandler      func(Ping, *Session)
}

// AddMessageHandler adds a function that will be called every time a message is received
func (s *Session) AddMessageHandler(fn func(Message, *Session)) {
	s.handlers.msgHandler = fn
}

// AddErrorHandler adds a function that will be called every time an error message is received
func (s *Session) AddErrorHandler(fn func(string, *Session)) {
	s.handlers.errHandler = fn
}

// AddJoinHandler adds a function that will be called every time a user join the chat
func (s *Session) AddJoinHandler(fn func(RoomAction, *Session)) {
	s.handlers.joinHandler = fn
}

// AddQuitHandler adds a function that will be called every time a user quits the chat
func (s *Session) AddQuitHandler(fn func(RoomAction, *Session)) {
	s.handlers.quitHandler = fn
}

// AddPMHandler adds a function that will be called every time a private message is received
func (s *Session) AddPMHandler(fn func(PrivateMessage, *Session)) {
	s.handlers.pmHandler = fn
}

// AddBroadcastHandler adds a function that will be called every time a broadcast is sent to the chat
func (s *Session) AddBroadcastHandler(fn func(Broadcast, *Session)) {
	s.handlers.broadcastHandler = fn
}

// AddPingHandler adds a function that will be called when a server responds with a pong
func (s *Session) AddPingHandler(fn func(Ping, *Session)) {
	s.handlers.pingHandler = fn
}
