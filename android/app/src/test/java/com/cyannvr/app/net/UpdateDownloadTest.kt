package com.cyannvr.app.net

import java.io.ByteArrayInputStream
import java.io.File
import java.io.IOException
import java.io.InputStream
import org.junit.Assert.*
import org.junit.Test

class UpdateDownloadTest {
    private fun temporaryFile() = File.createTempFile("cyannvr-update-", ".part")

    @Test fun reportsInitialAndCompletedProgress() {
        val target = temporaryFile()
        try {
            val events = mutableListOf<Pair<Long, Long>>()
            copyUpdatePackage(ByteArrayInputStream(ByteArray(1024)), target, 1024) { done, total -> events.add(done to total) }
            assertEquals(0L to 1024L, events.first())
            assertEquals(1024L to 1024L, events.last())
            assertEquals(1024L, target.length())
        } finally { target.delete() }
    }

    @Test fun unknownLengthReportsDownloadedBytes() {
        val target = temporaryFile()
        try {
            val events = mutableListOf<Pair<Long, Long>>()
            copyUpdatePackage(ByteArrayInputStream(ByteArray(512)), target, -1) { done, total -> events.add(done to total) }
            assertEquals(0L to -1L, events.first())
            assertEquals(512L to 512L, events.last())
        } finally { target.delete() }
    }

    @Test fun rejectsTruncatedPackageAndRemovesPartialFile() {
        val target = temporaryFile()
        assertThrows(IllegalStateException::class.java) {
            copyUpdatePackage(ByteArrayInputStream(ByteArray(10)), target, 20)
        }
        assertFalse(target.exists())
    }

    @Test fun rejectsEmptyPackage() {
        val target = temporaryFile()
        assertThrows(IllegalStateException::class.java) {
            copyUpdatePackage(ByteArrayInputStream(ByteArray(0)), target, -1)
        }
        assertFalse(target.exists())
    }

    @Test fun networkFailureClosesInputAndRemovesPartialFile() {
        val target = temporaryFile()
        var closed = false
        val input = object : InputStream() {
            override fun read(): Int = throw IOException("网络断开")
            override fun close() { closed = true }
        }
        assertThrows(IOException::class.java) { copyUpdatePackage(input, target, 100) }
        assertTrue(closed)
        assertFalse(target.exists())
    }
}
