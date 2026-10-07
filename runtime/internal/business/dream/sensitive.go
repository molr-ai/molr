package dream

import "regexp"

// Conservative exclusion of recognizable credential-bearing records. The model
// is also instructed not to infer or persist sensitive personal attributes.
var credentialPattern = regexp.MustCompile(`(?i)(api[_ -]?key|access[_ -]?token|secret|password|密码|验证码|密钥)\s*[:=：]|\b(sk-[A-Za-z0-9_-]{16,}|AKIA[A-Z0-9]{16}|Bearer\s+[A-Za-z0-9._-]{16,})|-----BEGIN .*PRIVATE KEY-----`)

func sensitive(text string) bool { return credentialPattern.MatchString(text) }
