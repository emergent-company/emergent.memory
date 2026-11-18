package main

// conversationStartedAt returns the created_at timestamp of the conversation
// the chat workspace is resuming, so the transcript can head itself with
// "Session started <date> <time>". It is empty when there is no active
// conversation or the list does not carry it (nil-safe: the rail list is
// best-effort). The client formats the ISO value in the browser's local
// timezone; the server only supplies the instant.
func conversationStartedAt(convs *ConversationList, id string) string {
	if convs == nil || id == "" {
		return ""
	}
	for _, c := range convs.Conversations {
		if c.ID == id {
			return c.CreatedAt
		}
	}
	return ""
}
