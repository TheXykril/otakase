package com.xyverion.otakase

import org.junit.Assert.*
import org.junit.Test

class UpdateSigningTest {
    @Test fun sameReleaseCertificatePassesWithoutLegacyLookup() {
        val release = setOf("release-certificate")
        verifyUpdateSigning(
            readUpdateSigners(release) { error("unexpected legacy lookup") },
            readUpdateSigners(release) { error("unexpected legacy lookup") },
        )
    }

    @Test fun missingArchiveSigningInfoCanUseMatchingLegacyCertificate() {
        val release = setOf("release-certificate")
        var reads = 0
        val archive = readUpdateSigners(emptySet()) { reads++; release }
        verifyUpdateSigning(release, archive)
        assertEquals(1, reads)
    }

    @Test fun missingInstalledSigningInfoCanUseMatchingLegacyCertificate() {
        val release = setOf("release-certificate")
        verifyUpdateSigning(readUpdateSigners(emptySet()) { release }, release)
    }

    @Test fun realMismatchIsNotOverriddenByLegacyCertificate() {
        val installed = setOf("release-certificate")
        val downloaded = readUpdateSigners(setOf("other-certificate")) {
            error("a real signer must not be overridden")
        }
        assertTrue(failure(installed, downloaded).contains("different signing key"))
    }

    @Test fun fallbackWithDifferentCertificateStillFails() {
        val downloaded = readUpdateSigners(emptySet()) { setOf("debug-certificate") }
        assertTrue(failure(setOf("release-certificate"), downloaded).contains("different signing key"))
    }

    @Test fun unreadableCertificatesAreNotCalledAKeyMismatch() {
        for ((installed, downloaded) in listOf(
            emptySet<String>() to setOf("release-certificate"),
            setOf("release-certificate") to emptySet<String>(),
            emptySet<String>() to emptySet<String>(),
        )) {
            val message = failure(installed, downloaded)
            assertTrue(message.contains("could not read"))
            assertFalse(message.contains("different signing key"))
        }
    }

    @Test fun everySignerMustMatchRegardlessOfOrder() {
        verifyUpdateSigning(setOf("a", "b"), setOf("b", "a"))
        assertTrue(failure(setOf("a", "b"), setOf("a")).contains("different signing key"))
    }

    private fun failure(installed: Set<String>, downloaded: Set<String>): String {
        try {
            verifyUpdateSigning(installed, downloaded)
            fail("verification should reject this update")
        } catch (e: IllegalArgumentException) {
            return e.message.orEmpty()
        }
        error("unreachable")
    }
}
