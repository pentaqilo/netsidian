package domain

import "strings"

// IsProviderStorageBinding distinguishes opaque provider bindings from local
// folder paths. A chosen folder may itself end in "-vault"; its namespace
// must be checked before interpreting a provider suffix.
func IsProviderStorageBinding(binding string) bool {
	return !strings.HasPrefix(binding, "local-folder:") && strings.HasSuffix(binding, "-vault")
}
