package utils

// ShortCommit safely shortens a commit hash to 8 characters or less
// Returns the original string if it's shorter than 8 characters
func ShortCommit(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}