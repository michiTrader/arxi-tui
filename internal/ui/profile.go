package ui

// Profile is how much colour the terminal can be trusted with. Downgrading is an
// emit concern and nothing else: a style names colours, and the resolver is the
// only code allowed to decide that 24-bit teal has to become colour 6 because we
// are in a 16-colour tty over ssh.
//
// The enum is copied and the downgrade arithmetic is not, on purpose: the
// resolver that will carry it (index collapsing, RGB quantisation) belongs to
// the engine's style half, where tokens are open and user-minted rather than a
// frozen theme table, and it lands with that phase instead of being lifted out
// of arxi-sim's emitter now.
type Profile uint8

const (
	ProfileTrueColor Profile = iota
	Profile256
	ProfileANSI
	ProfileMono
)
