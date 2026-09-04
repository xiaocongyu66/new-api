package identity

// OnResolveServerAddress returns the configured public server address, used to
// build absolute links (password reset) and to derive a Passkey Origin when the
// request carries no Host.
//
// The egress domain owns that setting and this domain cannot import it (that
// edge closes identity -> egress -> ... -> identity in the model/ test closure),
// so egress registers the hook from its own init(). egress does not reach
// identity, so that direction adds no cycle.
//
// The unregistered fallback repeats egress's own default rather than returning
// the empty string: egress.ServerAddress is a package var initialized to
// "http://localhost:3000", so before this inversion an unconfigured process
// still produced absolute links. Returning "" here would silently turn a
// password-reset link into a relative path in any binary that does not link
// egress.
var OnResolveServerAddress func() string

// DefaultServerAddress mirrors egress.ServerAddress's initial value. The two
// must stay in sync; egress asserts that in its own tests.
const DefaultServerAddress = "http://localhost:3000"

func serverAddress() string {
	if OnResolveServerAddress == nil {
		return DefaultServerAddress
	}
	return OnResolveServerAddress()
}
