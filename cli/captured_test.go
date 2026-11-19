package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestCapturedHoldsMoreThanAPipeDoes: captured read the pipe only after the
// command had returned, so a command that printed more than the pipe's buffer
// never returned. The buffer is 4 KiB on Windows, where a show --json example
// ran until the test binary was killed at six minutes. A megabyte is more
// than any platform's pipe holds, so this fails everywhere if the reading
// goes back to waiting for the writer.
func TestCapturedHoldsMoreThanAPipeDoes(t *testing.T) {
	line := strings.Repeat("x", 1023) + "\n"
	real := os.Stdout
	done := make(chan string, 1)
	go func() {
		out, _ := captured(t, func() error {
			for i := 0; i < 1024; i++ {
				fmt.Print(line)
			}
			return nil
		})
		done <- out
	}()
	select {
	case out := <-done:
		if len(out) != 1024*len(line) {
			t.Fatalf("captured %d bytes of %d", len(out), 1024*len(line))
		}
	case <-time.After(30 * time.Second):
		// The writer is still holding the pipe as standard output, and this
		// failure has to be printed somewhere that is read.
		os.Stdout = real
		t.Fatal("a megabyte of output was still being written after 30s: nothing is reading the pipe")
	}
}
