package ext

// This file holds installerNotice, the one piece of the community installer's
// scene construction shared between builds. The static InstallerScene that once
// baked each registry entry as a card retired when the keystroke loop landed
// (DESIGN-BLOCK-J.md J3 follow-up): the Scene 7 golden moved to LiveInstallerScene
// (registry_scene_live.go), which binds the entry list to the community.* view
// state the loop writes instead of baking it. installerNotice stays here because
// both the live installer and any future host-generated variant must carry it, so
// it is not owned by either builder.

// installerNotice is the gated diagnostic row every scene this product ships must
// carry (BINDS.md §2, PLAN.md invariant 3): the one node bound to host.scene.error,
// the field through which the host delivers every diagnostic it computes. The
// installer is a shipped, host-generated scene, so it is under the same obligation
// as SOBRIA or the mounted TICKER host — without it, a refusal or an engine
// diagnosis raised while the installer is on screen would be computed in full and
// discarded one function short of a pixel, indistinguishable from a crash. The
// `when` gate on the same field keeps it costless: host.scene.error is signed
// "text | null", so a clean browse renders zero rows for it and the golden does
// not move; it appears only when there is something to say.
func installerNotice() map[string]any {
	return map[string]any{
		"id":    "notice",
		"type":  "text",
		"bind":  "host.scene.error",
		"when":  "host.scene.error",
		"style": map[string]any{"style": "banner"},
	}
}
