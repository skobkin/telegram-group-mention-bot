package secretscrub

import "strings"

// TelegramTokenSecrets returns the registry entries for a Telegram bot token.
// Besides the full "<id>:<secret>" token, the secret part is registered on
// its own because it is the actually confidential half and may be logged
// separately from the numeric bot ID.
func TelegramTokenSecrets(token string) []Secret {
	secrets := []Secret{{Name: "telegram-token", Value: token}}

	if _, secretPart, ok := strings.Cut(token, ":"); ok && len(secretPart) >= 16 {
		secrets = append(secrets, Secret{Name: "telegram-token-secret", Value: secretPart})
	}

	return secrets
}
