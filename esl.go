package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
)

// eslClient is a minimal FreeSWITCH Event Socket Layer (ESL) client. It speaks
// the inbound protocol over any net.Conn, which keeps it decoupled from dialing
// and makes it testable over an in-memory net.Pipe.
type eslClient struct {
	conn  net.Conn
	input *textproto.Reader
}

// newESLClient wraps conn and performs the ESL auth handshake, returning a
// ready-to-use client or an error if authentication fails.
func newESLClient(conn net.Conn, password string) (*eslClient, error) {
	c := &eslClient{
		conn:  conn,
		input: textproto.NewReader(bufio.NewReader(conn)),
	}

	if err := c.auth(password); err != nil {
		return nil, err
	}

	return c, nil
}

// auth reads the initial auth/request and replies with the password, verifying
// that FreeSWITCH accepts it.
func (c *eslClient) auth(password string) error {
	message, err := c.input.ReadMIMEHeader()
	if err != nil {
		return fmt.Errorf("read auth failed: %w", err)
	}

	if message.Get("Content-Type") != "auth/request" {
		return errors.New("auth failed: unknown content-type")
	}

	if _, err := io.WriteString(c.conn, fmt.Sprintf("auth %s\n\n", password)); err != nil {
		return fmt.Errorf("write auth failed: %w", err)
	}

	message, err = c.input.ReadMIMEHeader()
	if err != nil {
		return fmt.Errorf("read auth failed: %w", err)
	}

	if message.Get("Content-Type") != "command/reply" {
		return errors.New("auth failed: unknown reply")
	}

	if reply := message.Get("Reply-Text"); reply != "+OK accepted" {
		return fmt.Errorf("auth failed: %s", reply)
	}

	return nil
}

// api runs an ESL command and returns the raw response body.
func (c *eslClient) api(command string) ([]byte, error) {
	if _, err := io.WriteString(c.conn, command+"\n\n"); err != nil {
		return nil, fmt.Errorf("cannot write command: %w", err)
	}

	message, err := c.input.ReadMIMEHeader()
	if err != nil {
		return nil, fmt.Errorf("cannot read command response: %w", err)
	}

	value := message.Get("Content-Length")
	length, err := strconv.Atoi(value)
	if err != nil {
		return nil, fmt.Errorf("invalid Content-Length %q: %w", value, err)
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(c.input.R, body); err != nil {
		return nil, err
	}

	return body, nil
}
