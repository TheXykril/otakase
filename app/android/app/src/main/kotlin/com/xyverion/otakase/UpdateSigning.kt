package com.xyverion.otakase

// Some Android firmware omits SigningInfo for an APK archive. Only consult
// the legacy package-manager result when the primary certificates are absent;
// an actual mismatch must never be replaced by a more convenient result.
internal fun readUpdateSigners(primary: Set<String>, legacy: () -> Set<String>): Set<String> =
    if (primary.isNotEmpty()) primary else legacy()

internal fun verifyUpdateSigning(installed: Set<String>, downloaded: Set<String>) {
    require(installed.isNotEmpty() && downloaded.isNotEmpty()) {
        "Android could not read the app signing certificates. Try installing the update from the release page."
    }
    require(installed == downloaded) {
        "This update uses a different signing key. Install updates from the same distributor."
    }
}
