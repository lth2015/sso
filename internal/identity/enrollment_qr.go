package identity

import (
	"encoding/base64"
	"net/url"

	qrcode "github.com/skip2/go-qrcode"
)

func provisioningURI(secret, account string) string {
	const issuer = "NSS SSO"
	u := url.URL{Scheme: "otpauth", Host: "totp", Path: "/" + issuer + ":" + account}
	u.RawQuery = url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}.Encode()
	return u.String()
}

// Generated in-process: enrollment secrets never reach a third-party QR service.
func enrollmentQR(secret, account string) (string, error) {
	png, err := qrcode.Encode(provisioningURI(secret, account), qrcode.Medium, 280)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
