// Package sshagent relays the ssh-agent protocol to a local agent with only
// the requests a remote ssh client needs: list keys, sign, and OpenSSH's
// session-bind. Requests that would change the agent (add or remove keys,
// lock it) never reach it.
package sshagent

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Message types from draft-miller-ssh-agent.
const (
	agentFailure          = 5
	agentRequestIdentites = 11
	agentSignRequest      = 13
	agentExtension        = 27
)

const maxMessage = 256 * 1024 // OpenSSH's own agent limit

var failureReply = []byte{0, 0, 0, 1, agentFailure}

// Hooks lets a caller watch the requests it relays.
type Hooks struct {
	// Sign runs before each sign request goes to the agent.
	Sign func()
	// Refused runs for each request kept from the agent, with its type.
	Refused func(msgType byte)
}

// Relay carries requests read from client to agent and the agent's replies
// back, one at a time, answering anything not allowed with a failure. It
// returns when either side closes.
func Relay(client io.ReadWriter, agent io.ReadWriter, hooks Hooks) error {
	for {
		msg, err := readMessage(client)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if !allowed(msg) {
			if hooks.Refused != nil {
				hooks.Refused(msg[0])
			}
			if _, err := client.Write(failureReply); err != nil {
				return err
			}
			continue
		}
		if msg[0] == agentSignRequest && hooks.Sign != nil {
			hooks.Sign()
		}
		if err := writeMessage(agent, msg); err != nil {
			return fmt.Errorf("write to agent: %w", err)
		}
		reply, err := readMessage(agent)
		if err != nil {
			return fmt.Errorf("read from agent: %w", err)
		}
		if err := writeMessage(client, reply); err != nil {
			return err
		}
	}
}

func allowed(msg []byte) bool {
	switch msg[0] {
	case agentRequestIdentites, agentSignRequest:
		return true
	case agentExtension:
		name, ok := readString(msg[1:])
		return ok && bytes.Equal(name, []byte("session-bind@openssh.com"))
	}
	return false
}

// readMessage reads one length-prefixed agent message, type byte first.
func readMessage(r io.Reader) ([]byte, error) {
	var n uint32
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return nil, err
	}
	if n == 0 || n > maxMessage {
		return nil, fmt.Errorf("agent message of %d bytes", n)
	}
	msg := make([]byte, n)
	if _, err := io.ReadFull(r, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func writeMessage(w io.Writer, msg []byte) error {
	buf := make([]byte, 4+len(msg))
	binary.BigEndian.PutUint32(buf, uint32(len(msg)))
	copy(buf[4:], msg)
	_, err := w.Write(buf)
	return err
}

// readString reads an ssh wire string: a uint32 length, then that many bytes.
func readString(b []byte) ([]byte, bool) {
	if len(b) < 4 {
		return nil, false
	}
	n := binary.BigEndian.Uint32(b)
	if uint32(len(b)-4) < n {
		return nil, false
	}
	return b[4 : 4+n], true
}
