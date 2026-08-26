import java.util.Base64

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
}

// ---------------------------------------------------------------------------
// Release signing
// ---------------------------------------------------------------------------
// Secrets are injected via environment variables (GitHub Actions → Secrets).
// Local developers can set these in ~/.gradle/gradle.properties or export
// them in the shell before running ./gradlew assembleRelease.
//
//   KEYSTORE_BASE64   — Base64-encoded JKS keystore file
//   STORE_PASSWORD    — keystore password
//   KEY_ALIAS         — key alias inside the keystore
//   KEY_PASSWORD      — private-key password
//
// If the variables are absent the release build is unsigned (F-Droid re-signs
// with its own key, so an unsigned APK is fine for F-Droid submission).
// ---------------------------------------------------------------------------
val keystoreB64: String? = System.getenv("KEYSTORE_BASE64")
val storePass:   String? = System.getenv("STORE_PASSWORD")
val keyAlias:    String? = System.getenv("KEY_ALIAS")
val keyPass:     String? = System.getenv("KEY_PASSWORD")

val hasSigningConfig = !keystoreB64.isNullOrBlank() &&
        !storePass.isNullOrBlank() &&
        !keyAlias.isNullOrBlank() &&
        !keyPass.isNullOrBlank()

android {
    namespace  = "com.freenet.vpn"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.freenet.vpn"
        minSdk    = 26       // Android 8.0 — minimum for reliable VpnService
        targetSdk = 35
        versionCode = 180
        versionName = "1.8.0"

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    if (hasSigningConfig) {
        // Write the decoded keystore to a temp file so Gradle can reference it.
        val keystoreFile = layout.buildDirectory.file("release.jks").get().asFile
        keystoreFile.parentFile.mkdirs()
        // getMimeDecoder() tolerates newlines produced by `base64` on Linux/macOS.
        keystoreFile.writeBytes(Base64.getMimeDecoder().decode(keystoreB64!!))

        signingConfigs {
            create("release") {
                storeFile     = keystoreFile
                storePassword = storePass
                this.keyAlias = keyAlias
                keyPassword   = keyPass
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
            if (hasSigningConfig) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
        debug {
            isDebuggable         = true
            applicationIdSuffix = ".debug"
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        compose = true
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }
}

dependencies {
    // Jetpack Compose BOM — all Compose versions aligned.
    val composeBom = platform(libs.androidx.compose.bom)
    implementation(composeBom)

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.ui)
    implementation(libs.androidx.ui.graphics)
    implementation(libs.androidx.ui.tooling.preview)
    implementation(libs.androidx.material3)
    implementation(libs.androidx.lifecycle.viewmodel.compose)

    debugImplementation(libs.androidx.ui.tooling)

    // Instrumented (on-device) tests.
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.androidx.test.rules)
    androidTestImplementation(libs.androidx.test.ext.junit)

    // FreeNet Go DPI bypass engine (AAR built via gomobile bind).
    // Run scripts/build-android.sh to generate app/libs/mobile.aar.
    if (file("libs/mobile.aar").exists()) {
        implementation(files("libs/mobile.aar"))
    }
}
