package auth

import (
	"context"

	"reelreview/internal/source"
)

// Provider isolates serving from the mechanism that validates account credentials.
// Token is a server-only Jellyfin token or an offline credential revision.
type Provider interface {
	Login(context.Context, string, string, string) (source.Account, string, error)
	Validate(context.Context, Session) (source.Account, error)
	Logout(context.Context, Session) error
	Mode() string
}

type Online struct{ Jellyfin *source.Jellyfin }

func (p Online) Mode() string { return "jellyfin" }
func (p Online) Login(ctx context.Context, name, password, device string) (source.Account, string, error) {
	return p.Jellyfin.Login(ctx, name, password, device)
}
func (p Online) Validate(ctx context.Context, s Session) (source.Account, error) {
	return p.Jellyfin.Me(ctx, s.Token, s.Device)
}
func (p Online) Logout(ctx context.Context, s Session) error {
	return p.Jellyfin.Call(ctx, "POST", "/Sessions/Logout", s.Token, s.Device, nil, nil)
}
