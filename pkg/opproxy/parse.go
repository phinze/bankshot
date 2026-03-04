package opproxy

import "strings"

// parseSubcommand extracts the first 1-2 non-flag tokens from args to form a
// subcommand string (e.g. "item get", "read", "whoami"). Returns "" if no
// subcommand is found.
func parseSubcommand(args []string) string {
	var tokens []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		tokens = append(tokens, arg)
		if len(tokens) == 2 {
			break
		}
	}

	// Two-word subcommands: "item get", "item list", "item create", etc.
	twoWord := map[string]bool{
		"item":    true,
		"account": true,
		"connect": true,
		"vault":   true,
		"group":   true,
		"user":    true,
		"plugin":  true,
	}

	if len(tokens) >= 2 && twoWord[tokens[0]] {
		return tokens[0] + " " + tokens[1]
	}
	if len(tokens) >= 1 {
		return tokens[0]
	}
	return ""
}

// parseVault extracts the vault name from args. It checks:
//  1. --vault <name> or --vault=<name> flags
//  2. op:// URI in the first non-flag arg (for "read" subcommand)
//
// Returns "" if no vault is found.
func parseVault(args []string) string {
	for i, arg := range args {
		if arg == "--vault" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--vault=") {
			return strings.TrimPrefix(arg, "--vault=")
		}
	}

	// Check for op:// URI in positional args
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.HasPrefix(arg, "op://") {
			return parseVaultFromURI(arg)
		}
	}

	return ""
}

// parseVaultFromURI extracts the vault name from an op:// URI.
// Format: op://Vault/Item[/Field]
func parseVaultFromURI(uri string) string {
	trimmed := strings.TrimPrefix(uri, "op://")
	parts := strings.SplitN(trimmed, "/", 3)
	if len(parts) >= 1 && parts[0] != "" {
		return parts[0]
	}
	return ""
}
