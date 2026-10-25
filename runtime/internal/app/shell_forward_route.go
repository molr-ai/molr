package app

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/portforward"
)

func (s *sandboxRouter) dialForward(ctx context.Context, e core.Execution, id core.ID, port int) (portforward.Stream, error) {
	if id == "local" {
		if s.shell == nil {
			return nil, core.NewError(core.Unavailable, "local sandbox unavailable")
		}
		conn, err := s.shell.runner.DialForward(ctx, e, port)
		if err != nil {
			return nil, err
		}
		stream, ok := conn.(portforward.Stream)
		if !ok {
			conn.Close()
			return nil, fmt.Errorf("TCP half-close unavailable")
		}
		return stream, nil
	}
	connection, err := s.connection(ctx, e.Principal, id)
	if err != nil {
		return nil, err
	}
	client, err := s.client(ctx, e.Principal, connection)
	if err != nil {
		return nil, err
	}
	return client.DialForward(ctx, identity(e), port)
}
