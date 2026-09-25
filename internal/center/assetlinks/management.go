package assetlinks

// GlobalActionConfirmation binds an explicitly authorized shared action to the
// management review the operator saw. Single-parent actions need no extra grant.
type GlobalActionConfirmation struct {
	PreviewDigest       string `json:"preview_digest"`
	ConfirmSharedImpact bool   `json:"confirm_shared_impact"`
}
