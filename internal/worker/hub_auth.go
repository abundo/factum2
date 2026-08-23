package worker

import "context"

type hubAuthKey struct{}

// WithHubAuth marks ctx as a hub-authenticated in-process request.
// Network clients cannot set this; it is equivalent to a service token,
// not an admin session.
func WithHubAuth(ctx context.Context) context.Context {
	return context.WithValue(ctx, hubAuthKey{}, true)
}

func IsHubAuth(ctx context.Context) bool {
	v, _ := ctx.Value(hubAuthKey{}).(bool)
	return v
}
