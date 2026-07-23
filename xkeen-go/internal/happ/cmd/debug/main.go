package main

import (
	"fmt"
	"os"

	"github.com/fan92rus/xkeen-ui/internal/happ"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: happ-debug <happ://crypt5/...>")
		os.Exit(1)
	}

	d, err := happ.NewDecryptorEmbedded()
	if err != nil {
		fmt.Fprintln(os.Stderr, "decryptor:", err)
		os.Exit(1)
	}

	url, err := d.Decrypt(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "decrypt:", err)
		os.Exit(1)
	}

	fmt.Print(url)
}
