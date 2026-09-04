package identity

// Test-only accessors for the two seams identity cannot fill itself. They live
// in a _test.go file so the production API stays unchanged: only the external
// identity_test package (which can import the registering packages) uses them.

// RedeemForTest calls the registered redemption hook, panicking on nil exactly
// as TopUp does.
func RedeemForTest(key string, userID int) (int, error) { return redeemKey(key, userID) }

// OAuthRegistrarForTest returns the registered custom OAuth registrar, or nil if
// no package registered one.
func OAuthRegistrarForTest() CustomOAuthRegistrar { return oauthRegistrar }
