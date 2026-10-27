import java.util.Properties

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

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
                abiFilters.addAll(listOf("armeabi-v7a", "arm64-v8a"))
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

    buildTypes {
        release {
            signingConfig = signingConfigs.getByName(if (releaseKey) "release" else "debug")
        }
    }
}

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
