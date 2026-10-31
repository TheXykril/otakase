import java.net.URI
import java.security.MessageDigest
import java.util.Properties
import java.util.zip.ZipFile

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

val fullMpvDir = layout.buildDirectory.dir("full-mpv/jniLibs")

android {
    namespace = "com.xyverion.otakase"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = "28.2.13676358"

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        applicationId = "com.xyverion.otakase"
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        // Third-party player libraries also ship x86 binaries. The universal
        // spike APK contains only the same two ARM ABIs as the Go binding.
        // Splits configure their own filters through the Flutter plugin.
        if (project.findProperty("split-per-abi")?.toString() != "true") {
            ndk {
                abiFilters.clear()
                // flutter run selects this target on an Intel TV emulator.
                if (project.findProperty("target-platform")?.toString() == "android-x64") {
                    abiFilters.add("x86_64")
                } else {
                    abiFilters.addAll(listOf("armeabi-v7a", "arm64-v8a"))
                }
            }
        }
    }

    // Release builds are signed with the Otakase key named in
    // android/key.properties (never committed). Without that file they fall
    // back to the local debug key, which cannot update an installed release.
    val keyFile = rootProject.file("key.properties")
    val releaseKey = keyFile.exists()
    signingConfigs {
        if (releaseKey) {
            val key = Properties().apply { keyFile.inputStream().use { load(it) } }
            create("release") {
                storeFile = rootProject.file(key.getProperty("storeFile"))
                storePassword = key.getProperty("storePassword")
                keyAlias = key.getProperty("keyAlias")
                keyPassword = key.getProperty("keyPassword")
            }
        }
    }

    // media_kit's libmpv has no audio filters, so the volume leveller (mpv
    // dynaudnorm) would do nothing. The player uses the full ffmpeg/mpv
    // libraries from mpv-android's reproducible F-Droid build instead. Our
    // copy of each library wins over the one media_kit ships.
    sourceSets.getByName("main").jniLibs.srcDir(fullMpvDir.get().asFile)
    packaging {
        jniLibs {
            pickFirsts += listOf("**/libmpv.so", "**/libc++_shared.so")
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.getByName(if (releaseKey) "release" else "debug")
        }
    }
}

// mpv-android 2026-09-17 (F-Droid versionCode 48). Bump both values together.
val fullMpvApk = "https://f-droid.org/repo/is.xyz.mpv_48.apk"
val fullMpvSha256 = "5cba2237b20ba34b79eaccfbd39bdb5b09d2a9a774bdccee09ca450eac4edd34"

val fetchFullMpv by tasks.registering {
    val out = fullMpvDir
    outputs.dir(out)
    inputs.property("sha256", fullMpvSha256)
    doLast {
        val cache = File(gradle.gradleUserHomeDir, "otakase/mpv-android-$fullMpvSha256.apk")
        fun digest(f: File) = MessageDigest.getInstance("SHA-256")
            .digest(f.readBytes()).joinToString("") { "%02x".format(it) }
        if (!cache.exists() || digest(cache) != fullMpvSha256) {
            cache.parentFile.mkdirs()
            val part = File(cache.path + ".part")
            URI(fullMpvApk).toURL().openStream().use { input ->
                part.outputStream().use { input.copyTo(it) }
            }
            check(digest(part) == fullMpvSha256) { "mpv-android APK checksum mismatch" }
            part.renameTo(cache)
        }
        val dir = out.get().asFile
        dir.deleteRecursively()
        ZipFile(cache).use { zip ->
            for (entry in zip.entries()) {
                val parts = entry.name.split('/')
                // libplayer.so is mpv-android's own JNI glue, not needed here.
                if (parts.size != 3 || parts[0] != "lib" || !parts[2].endsWith(".so") ||
                    parts[2] == "libplayer.so" ||
                    parts[1] !in listOf("armeabi-v7a", "arm64-v8a", "x86_64")
                ) continue
                val target = File(dir, "${parts[1]}/${parts[2]}")
                target.parentFile.mkdirs()
                zip.getInputStream(entry).use { input ->
                    target.outputStream().use { input.copyTo(it) }
                }
            }
        }
    }
}

tasks.matching { it.name.startsWith("merge") && it.name.endsWith("JniLibFolders") }
    .configureEach { dependsOn(fetchFullMpv) }

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}

dependencies {
    implementation(files("libs/mobilecore.aar"))
}
