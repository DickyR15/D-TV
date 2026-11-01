package com.dickyr15.dtv

import android.content.pm.ActivityInfo

/**
 * Dedicated Android TV launcher that keeps the system splash and Flutter
 * startup in landscape, even while device-mode detection is still running.
 */
class TvMainActivity : MainActivity() {
    override fun setRequestedOrientation(requestedOrientation: Int) {
        // Flutter can request an unspecified or portrait orientation briefly
        // before TV detection completes. Never allow the TV entry to rotate.
        super.setRequestedOrientation(ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE)
    }
}
