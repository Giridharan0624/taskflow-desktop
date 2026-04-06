package auth

// tokenMeta stores metadata alongside keyring tokens.
type tokenMeta struct {
	ExpiresAt int64 `json:"expiresAt"`
}
