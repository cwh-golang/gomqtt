// Package broker implements an extensible MQTT broker.
package broker

import (
	"net"
	"sync"
	"time"

	"github.com/256dpi/gomqtt/packet"
	"github.com/256dpi/gomqtt/transport"

	"gopkg.in/tomb.v2"
)

// LogEvent are received by a Logger.
type LogEvent string

const (
	// NewConnection is emitted when a client comes online.
	NewConnection LogEvent = "new connection"

	// PacketReceived is emitted when a packet has been received.
	PacketReceived LogEvent = "packet received"

	// MessagePublished is emitted after a message has been published.
	MessagePublished LogEvent = "message published"

	// MessageAcknowledged is emitted after a message has been acknowledged.
	MessageAcknowledged LogEvent = "message acknowledged"

	// MessageForwarded is emitted after a message has been forwarded.
	MessageForwarded LogEvent = "message forwarded"

	// PacketSent is emitted when a packet has been sent.
	PacketSent LogEvent = "packet sent"

	// ClientDisconnected is emitted when a client disconnects cleanly.
	ClientDisconnected LogEvent = "client disconnected"

	// TransportError is emitted when an underlying transport error occurs.
	TransportError LogEvent = "transport error"

	// SessionError is emitted when a call to the session fails.
	SessionError LogEvent = "session error"

	// BackendError is emitted when a call to the backend fails.
	BackendError LogEvent = "backend error"

	// ClientError is emitted when the client violates the protocol.
	ClientError LogEvent = "client error"

	// LostConnection is emitted when the connection has been terminated.
	LostConnection LogEvent = "lost connection"
)

// The Logger callback handles incoming log messages.
type Logger func(LogEvent, *Client, packet.GenericPacket, *packet.Message, error)

// The Engine handles incoming connections and connects them to the backend.
type Engine struct {
	// The Backend that will passed to accepted clients.
	Backend Backend

	// The logger that will be passed to accepted clients.
	Logger Logger

	// ConnectTimeout defines the timeout to receive the first packet.
	ConnectTimeout time.Duration

	// The Default* properties will be set on newly accepted connections.
	DefaultReadLimit   int64
	DefaultReadBuffer  int
	DefaultWriteBuffer int

	// OnError can be used to receive errors from engine. If an error is received
	// the server should be restarted.
	OnError func(error)

	mutex sync.Mutex
	tomb  tomb.Tomb
}

// NewEngine returns a new Engine.
func NewEngine(backend Backend) *Engine {
	return &Engine{
		Backend:        backend,
		ConnectTimeout: 10 * time.Second,
		//Logger: func(e LogEvent, _ *Client, p packet.GenericPacket, m *packet.Message, err error) {
		//	pretty.Println(e, p, m, err)
		//},
	}
}

// Accept begins accepting connections from the passed server.
func (e *Engine) Accept(server transport.Server) {
	e.tomb.Go(func() error {
		for {
			// return if dying
			if !e.tomb.Alive() {
				return tomb.ErrDying
			}

			// accept next connection
			conn, err := server.Accept()
			if err != nil {
				// call error callback if available
				if e.OnError != nil {
					e.OnError(err)
				}

				return err
			}

			// handle connection
			if !e.Handle(conn) {
				return nil
			}
		}
	})
}

// Handle takes over responsibility and handles a transport.Conn. It returns
// false if the engine is closing and the connection has been closed.
func (e *Engine) Handle(conn transport.Conn) bool {
	// check conn
	if conn == nil {
		panic("passed conn is nil")
	}

	// acquire mutex
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// close conn immediately when dying
	if !e.tomb.Alive() {
		conn.Close()
		return false
	}

	// set default read limit
	conn.SetReadLimit(e.DefaultReadLimit)

	// TODO: Buffers should be configured before the socket is opened.
	// go1.11 should provide a custom dialer facility that might allow this

	// set default buffer sizes
	conn.SetBuffers(e.DefaultReadBuffer, e.DefaultWriteBuffer)

	// set initial read timeout
	conn.SetReadTimeout(e.ConnectTimeout)

	// handle client
	NewClient(e.Backend, e.Logger, conn)

	return true
}

// Close will stop handling incoming connections and close all current clients.
// The call will block until all clients are properly closed.
//
// Note: All passed servers to Accept must be closed before calling this method.
func (e *Engine) Close() {
	// acquire mutex
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// stop acceptors
	e.tomb.Kill(nil)
	e.tomb.Wait()
}

// Run runs the passed engine on a random available port and returns a channel
// that can be closed to shutdown the engine. This method is intended to be used
// in testing scenarios.
func Run(engine *Engine, protocol string) (string, chan struct{}, chan struct{}) {
	// launch server
	server, err := transport.Launch(protocol + "://localhost:0")
	if err != nil {
		panic(err)
	}

	// prepare channels
	quit := make(chan struct{})
	done := make(chan struct{})

	// start accepting connections
	engine.Accept(server)

	// prepare shutdown
	go func() {
		// wait for signal
		<-quit

		// errors from close are ignored
		server.Close()

		// close broker
		engine.Close()

		close(done)
	}()

	// get random port
	_, port, _ := net.SplitHostPort(server.Addr().String())

	return port, quit, done
}
