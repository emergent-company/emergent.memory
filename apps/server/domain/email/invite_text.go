package email

import (
	"fmt"
	"strings"
)

// ProjectInvitationPlainText builds the plain-text alternative body for a
// project-invitation email. It mirrors the content of the project-invitation
// template so that clients which cannot render HTML still receive the accept
// link and the expiry. The email deliberately carries no CLI instructions: the
// invitation is accepted in the browser, and the product surfaces pending
// invitations in the app as well (so a recipient who is already signed in never
// has to go through the CLI to discover it).
func ProjectInvitationPlainText(inviterName, projectName, roleLabel, acceptURL string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Hi there,\n\n")
	fmt.Fprintf(&b, "%s has invited you to join the project %s as a %s.\n\n", inviterName, projectName, roleLabel)
	fmt.Fprintf(&b, "Accept invitation: %s\n\n", acceptURL)
	b.WriteString("This invitation expires in 7 days.\n\n")
	b.WriteString("Accept directly in your browser using the link above.\n\n")
	b.WriteString("If you weren't expecting this invitation, you can safely ignore this email.\n\n")
	b.WriteString("— The emergent.memory team\n\n")
	fmt.Fprintf(&b, "Invitation link: %s\n", acceptURL)

	return b.String()
}
