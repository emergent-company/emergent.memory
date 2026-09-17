// Package components holds Memory's shared, generic gateway view components.
//
// Contract: everything in this package renders markup and nothing else. It must
// not import a gateway domain type, embed a gateway route path, or read gateway
// configuration. Anything that needs a domain vocabulary — mapping a status to a
// badge intent, building a resource URL, deciding which noun a secret notice
// uses — stays in package main as a thin adapter that calls into this package.
//
// The package exists because the same markup was previously maintained in
// parallel copies across page templates, and had already drifted. A shared
// component makes a pattern change a single compiler-checked edit.
package components
