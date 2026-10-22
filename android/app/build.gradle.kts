// The Android app: the desktop app's page in a WebView, a remote for a Kitties gateway that runs
// on a computer or a server. The page is built by desktop/frontend (npm run build) and packed as
// the app's assets.
plugins {
    id("com.android.application")
}

// The version comes from the release tag (KITTIES_VERSION=v0.1.7), so the APK matches the release.
val tag = (System.getenv("KITTIES_VERSION")?.takeIf { it.isNotBlank() } ?: "v0.0.0").removePrefix("v")
val parts = tag.split("-")[0].split(".").map { it.toIntOrNull() ?: 0 } + listOf(0, 0, 0)

android {
    namespace = "app.kitties"
    compileSdk = 35

    defaultConfig {
        applicationId = "app.kitties"
        minSdk = 24
        targetSdk = 35
        versionCode = parts[0] * 10000 + parts[1] * 100 + parts[2] + 1
        versionName = tag
    }

    // A release signed with the key in KITTIES_KEYSTORE_FILE (a CI secret), or with the debug key
    // when there is none: either installs; only the same key can update an installed app.
    fun env(name: String): String? = System.getenv(name)?.takeIf { it.isNotBlank() }
    val keystore = env("KITTIES_KEYSTORE_FILE")
    signingConfigs {
        if (keystore != null) {
            create("release") {
                storeFile = file(keystore)
                storePassword = env("KITTIES_KEYSTORE_PASSWORD")
                keyAlias = env("KITTIES_KEY_ALIAS") ?: "kitties"
                keyPassword = env("KITTIES_KEY_PASSWORD") ?: env("KITTIES_KEYSTORE_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = if (keystore != null) signingConfigs.getByName("release") else signingConfigs.getByName("debug")
        }
    }

    sourceSets {
        getByName("main") {
            assets.srcDirs("../../desktop/frontend/dist")
        }
    }

    // The release build skips lint's own download of its rules; CI checks the page, not this shell.
    lint {
        checkReleaseBuilds = false
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation("androidx.webkit:webkit:1.12.1")
}
