// Package tooloauth gives agent tools the end user's OAuth token. It is an
// ADK runner plugin that serves a stored token, asks the user for consent
// mid-run when there is none, and stores the tokens it obtains through ADK's
// [auth.CredentialStore]. It speaks the wire format of ADK's toolauth types,
// so AG-UI and ADK Web clients see the same auth config adk-python sends.
package tooloauth
