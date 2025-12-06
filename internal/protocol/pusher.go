package protocol

// Standard Pusher Protocol v7 Events
const (
	// Client -> Server
	EventPing        = "pusher:ping"
	EventPong        = "pusher:pong"
	EventSubscribe   = "pusher:subscribe"
	EventUnsubscribe = "pusher:unsubscribe"

	// Server -> Client
	EventConnectionEstablished = "pusher:connection_established"
	EventError                 = "pusher:error"
	EventSubscriptionSucceeded = "pusher_internal:subscription_succeeded"
	EventMemberAdded           = "pusher_internal:member_added"
	EventMemberRemoved         = "pusher_internal:member_removed"
)

// Channel Prefixes
const (
	ChannelPrefixPrivate  = "private-"
	ChannelPrefixPresence = "presence-"
	ChannelPrefixClient   = "client-"
)

// Error Codes
const (
	ErrorSubscriptionDenied = 4009
)

// Limits
const (
	MaxChannelLength = 256
	MaxEventLength   = 64
	MaxDataSize      = 256 * 1024
)
