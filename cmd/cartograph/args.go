package main

import "strings"

// splitPositional pulls the first n tokens that do not look like a flag
// (i.e. do not start with "-") out of args, in order, and returns them
// alongside every remaining token (flags and their values) untouched and
// in its original relative order, ready to hand to a flag.FlagSet. None of
// this program's flags are boolean, so a "-name" token not in "-name=value"
// form is always assumed to consume the next token as its value.
//
// This lets every subcommand accept its positional arguments before,
// after, or interleaved with its flags, matching this card's own usage
// lines (for example "cartograph import <dir> -db cartograph.db --actor ...",
// positional first) without depending on the standard flag package's
// flags-before-positionals-only parsing.
func splitPositional(args []string, n int) (positional []string, rest []string) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			rest = append(rest, a)
			if !strings.Contains(a, "=") && i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
			continue
		}
		if len(positional) < n {
			positional = append(positional, a)
			continue
		}
		rest = append(rest, a)
	}
	return positional, rest
}
