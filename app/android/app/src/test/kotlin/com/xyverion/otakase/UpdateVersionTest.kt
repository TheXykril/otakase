package com.xyverion.otakase

import org.junit.Assert.*
import org.junit.Test

class UpdateVersionTest {
    @Test fun higherCodeIsNewer() {
        assertTrue(isNewerUpdate(26008000, "26.8.0", 26007000, "26.7.0"))
        assertFalse(isNewerUpdate(26007000, "26.7.0", 26008000, "26.8.0-dev.3"))
    }

    @Test fun laterDevBuildOfTheSameVersionIsNewer() {
        assertTrue(isNewerUpdate(26008000, "26.8.0-dev.42", 26008000, "26.8.0-dev.41"))
        assertFalse(isNewerUpdate(26008000, "26.8.0-dev.41", 26008000, "26.8.0-dev.42"))
        assertFalse(isNewerUpdate(26008000, "26.8.0-dev.41", 26008000, "26.8.0-dev.41"))
    }

    @Test fun releaseReplacesItsDevBuilds() {
        assertTrue(isNewerUpdate(26008000, "26.8.0", 26008000, "26.8.0-dev.41"))
        assertFalse(isNewerUpdate(26008000, "26.8.0-dev.50", 26008000, "26.8.0"))
        assertFalse(isNewerUpdate(26008000, "26.8.0", 26008000, "26.8.0"))
    }

    @Test fun unreadableNamesNeedAHigherCode() {
        assertFalse(isNewerUpdate(26008000, "26.8.0", 26008000, "local"))
        assertFalse(isNewerUpdate(26008000, null, 26008000, "26.8.0-dev.1"))
    }
}
