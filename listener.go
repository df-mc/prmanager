package main

import (
	"fmt"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/sandertv/gophertunnel/minecraft/text"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"
)

// Listener wraps a minecraft.Listener that accepts connections before transferring them to a new destination
// server based on the address that was used to join.
type Listener struct {
	docker   *Docker
	listener *minecraft.Listener

	lastConnections map[string]time.Time
	killChan        chan struct{}
}

// NewListener creates a new Listener using the provided Docker instance.
func NewListener(docker *Docker) *Listener {
	return &Listener{
		docker: docker,

		lastConnections: make(map[string]time.Time),
	}
}

// Listen starts listening for clients to accept and handle once they have joined.
func (l *Listener) Listen(addr string) error {
	slog.Info("Starting Minecraft listener", "addr", addr)
	listener, err := minecraft.Listen("raknet", addr)
	if err != nil {
		return err
	}
	l.listener = listener

	for {
		c, err := listener.Accept()
		if err != nil {
			if l.listener == nil {
				return nil
			}
			fmt.Println("Error accepting connection:", err)
			continue
		}
		l.handleConnection(c.(*minecraft.Conn))
	}
}

// handleConnection handles a new connection to the Listener. It reads the client's server address and
// determines the correct port to redirect the client to.
func (l *Listener) handleConnection(c *minecraft.Conn) {
	logger := slog.Default().With(slog.Group(
		"connection",
		slog.String("xuid", c.IdentityData().XUID),
		slog.String("identity", c.IdentityData().Identity),
		slog.String("display_name", c.IdentityData().DisplayName),
		slog.String("server_address", c.ClientData().ServerAddress),
	))
	logger.Info("Accepted connection")

	// Although it takes some time, we need to let the client fully connect before we can transfer them.
	err := c.StartGame(minecraft.GameData{})
	if err != nil {
		logger.Error("Failed to start game", slog.Any("error", err))
		_ = l.listener.Disconnect(c, text.Colourf("<red>Failed to start game</red>"))
		return
	}

	// Try and find the correct port to redirect the client to. It can either be a fixed port for the main and
	// plots server, or it can be a pull request that is running on a random port.
	var targetPort uint16
	addr := strings.Split(c.ClientData().ServerAddress, ":")[0]
	if addr == "df-mc.dev" || addr == "188.166.78.44" {
		targetPort = 19133
	} else if addr == "plots.df-mc.dev" {
		targetPort = 19134
	} else {
		// Assuming the address is in the format of a pull request, e.g., "123.df-mc.dev".
		var regex = `^(\d+)\.df-mc\.dev$`
		if matches := regexp.MustCompile(regex).FindStringSubmatch(addr); len(matches) > 1 {
			// Check if the pull request exists on the host.
			pr := matches[1]
			if _, err = os.Stat("pr-" + pr); err != nil {
				logger.Error("Pull request directory does not exist", slog.String("pr", pr), slog.Any("error", err))
				_ = l.listener.Disconnect(c, text.Colourf("<red>Invalid or outdated pull request</red>"))
				return
			}

			// Try obtaining the server port for the pull request if the server is already running.
			port, found, err := l.docker.ServerPort(pr)
			if err != nil {
				logger.Error("Failed to get server port", slog.String("pr", pr), slog.Any("error", err))
				_ = l.listener.Disconnect(c, text.Colourf("<red>Failed to get server port</red>"))
				return
			} else if !found {
				// The server is not running, so we need to start it.
				port, found, err = l.docker.StartServer(pr)
				if err != nil {
					logger.Error("Failed to start server", slog.String("pr", pr), slog.Any("error", err))
					_ = l.listener.Disconnect(c, text.Colourf("<red>Failed to start server</red>"))
					return
				} else if !found {
					logger.Info("Server not found for PR", slog.String("pr", pr))
					_ = l.listener.Disconnect(c, text.Colourf("<red>Server not found for PR %s</red>", pr))
					return
				}
				slog.Info("Started server for PR", slog.String("pr", pr), slog.Int("port", int(port)))
			} else {
				slog.Info("Found existing server for PR", slog.String("pr", pr), slog.Int("port", int(port)))
			}
			targetPort = port
			l.lastConnections[pr] = time.Now()
		} else {
			// Server address is not in the expected format.
			logger.Info("Invalid server address", slog.String("address", addr))
			_ = l.listener.Disconnect(c, text.Colourf("<red>Invalid server address: %s</red>", addr))
			return
		}
	}
	if targetPort == 0 {
		// Should not be possible but just in case the port is not set for some reason.
		logger.Error("Failed to determine target port")
		_ = l.listener.Disconnect(c, text.Colourf("<red>Failed to determine target port</red>"))
		return
	}

	// Finally redirect the connection to the target port.
	logger.Info("Redirecting connection", slog.Int("target_port", int(targetPort)))
	_ = c.WritePacket(&packet.Transfer{
		Address: "df-mc.dev",
		Port:    targetPort,
	})
}

// KillInactiveServers periodically checks for inactive servers and stops them if they have not been connected
// to for more than an hour.
func (l *Listener) KillInactiveServers() {
	t := time.NewTicker(time.Minute * 5)
	for {
		select {
		case <-t.C:
			for pr, lastConn := range l.lastConnections {
				if time.Since(lastConn) > time.Hour {
					slog.Info("Killing inactive server", slog.String("pr", pr))
					l.docker.StopServer(pr)
					delete(l.lastConnections, pr)
				}
			}
		case <-l.killChan:
			t.Stop()
			return
		}
	}
}

// Close closes the listener and stops accepting new connections.
func (l *Listener) Close() {
	if l.listener != nil {
		_ = l.listener.Close()
		l.listener = nil
	}
	if l.killChan != nil {
		close(l.killChan)
	}
}
