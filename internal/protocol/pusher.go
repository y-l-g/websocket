package protocol

import "regexp"

// Standard Pusher Protocol v7 Events
const (
	// Client -> Server
	EventPing        = "pusher:ping"
	EventPong        = "pusher:pong"
	EventSubscribe   = "pusher:subscribe"
	EventUnsubscribe = "pusher:unsubscribe"
	EventSignin      = "pusher:signin" // Added

	// Server -> Client
	EventConnectionEstablished = "pusher:connection_established"
	EventError                 = "pusher:error"
	EventSubscriptionSucceeded = "pusher_internal:subscription_succeeded"
	EventMemberAdded           = "pusher_internal:member_added"
	EventMemberRemoved         = "pusher_internal:member_removed"
	EventSigninSuccess         = "pusher:signin_success" // Added
)

// Channel Prefixes
const (
	ChannelPrefixPrivate  = "private-"
	ChannelPrefixPresence = "presence-"
	ChannelPrefixClient   = "client-"
)

// Error Codes
const (
	ErrorApplicationDisabled = 4003 // Used for Binary frames (Generic Not Supported) or disabled app
	ErrorOverCapacity        = 4100
	ErrorGenericReconnect    = 4200
	ErrorUnsupportedProtocol = 4007
	ErrorSubscriptionDenied  = 4009
	ErrorSigninLimitExceeded = 4302 // Watchlist limit
)

// Limits
const (
	MaxChannelLength = 256
	MaxEventLength   = 64
	MaxDataSize      = 256 * 1024
)

var validChannelName = regexp.MustCompile(`^[a-zA-Z0-9_\-=@,.;]+$`)

// IsValidChannelName checks if the channel name adheres to the Pusher spec.
func IsValidChannelName(name string) bool {
	if len(name) == 0 || len(name) > MaxChannelLength {
		return false
	}
	return validChannelName.MatchString(name)
}
