package com.xyverion.otakase

// Release versions look like 26.7.0 (versionCode 26007000). Dev builds look
// like 26.8.0-dev.41: a test build of the version they are heading to, with
// that version's code. Dev builds of one version and its release share a
// versionCode, which Android installs over each other, so the name decides
// which is newer: dev.41 < dev.42 < the release.

private val versionPattern = Regex("""^(\d{1,3})\.(\d{1,3})(?:\.(\d{1,3}))?(?:-dev\.(\d{1,9}))?$""")

// The dev build number, Long.MAX_VALUE for a release, or null when the name
// is not a version.
internal fun devNumber(name: String?): Long? {
    val match = versionPattern.matchEntire(name?.trim() ?: return null) ?: return null
    return match.groupValues[4].takeIf { it.isNotEmpty() }?.toLong() ?: Long.MAX_VALUE
}

// True when the downloaded package (code, name) is newer than the installed
// one. A higher versionCode always is; an equal one only when both names
// are versions and the download's is later.
internal fun isNewerUpdate(code: Long, name: String?, installedCode: Long, installedName: String?): Boolean {
    if (code != installedCode) return code > installedCode
    val next = devNumber(name) ?: return false
    val current = devNumber(installedName) ?: return false
    return next > current
}
