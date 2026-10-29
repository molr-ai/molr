package channelstate

// Summary is the binding information needed to choose or manage a channel.
// Channel is webui until pairing confirms the external chat identity.
type Summary struct {
	Channel string
	State   string
}
