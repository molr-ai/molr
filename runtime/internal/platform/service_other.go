//go:build !windows

package platform

import "context"

func ServiceContext(ctx context.Context, directory string) (context.Context, func(), error) {
	return ctx, func() {}, nil
}
