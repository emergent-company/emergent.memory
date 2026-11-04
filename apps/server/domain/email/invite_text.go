package email

import (
	"fmt"
	"strings"
)

// ProjectInvitationPlainText builds the plain-text alternative body for a
// project-invitation email. It mirrors the content of the project-invitation
// template so that clients which cannot render HTML still receive the accept
// link and the CLI install instructions (including the install.sh one-liner).
func ProjectInvitationPlainText(inviterName, projectName, roleLabel, acceptURL string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Hi there,\n\n")
	fmt.Fprintf(&b, "%s has invited you to join the project %s as a %s.\n\n", inviterName, projectName, roleLabel)
	fmt.Fprintf(&b, "Accept invitation: %s\n\n", acceptURL)
	b.WriteString("This invitation expires in 7 days.\n\n")
	b.WriteString("New to emergent.memory? Get started in 3 steps:\n")
	b.WriteString("1. Install the CLI\n")
	b.WriteString("   curl -fsSL https://get.emergent.memory/install.sh | sh\n")
	b.WriteString("2. Log in\n")
	b.WriteString("   memory login\n")
	b.WriteString("3. After accepting, connect to the project\n")
	fmt.Fprintf(&b, "   memory projects set %s\n\n", projectName)
	b.WriteString("Or accept directly in the browser using the link above.\n\n")
	b.WriteString("If you weren't expecting this invitation, you can safely ignore this email.\n\n")
	b.WriteString("— The emergent.memory team\n\n")
	fmt.Fprintf(&b, "Invitation link: %s\n", acceptURL)

	return b.String()
}
