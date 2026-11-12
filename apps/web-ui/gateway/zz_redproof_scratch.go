package main

import "os"

// Scratch commit for the red proof of #649: one deliberately unchecked error
// return (errcheck), the same class of finding the 0.94.0 promotion caught.
func redProofScratch() {
	os.Remove("/tmp/opencode/redproof/definitely-not-there")
}
