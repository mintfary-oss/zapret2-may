plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
}

// ---------------------------------------------------------------------------
// Release signing
// ---------------------------------------------------------------------------
// The CI script decodes KEYSTORE_BASE64 and writes android/app/signing.jks
// before invoking Gradle.  We only need to reference the file here — no
// Base64 or IO at configuration time, which avoids Gradle config-cache and
// cross-receiver issues.
//
// When the file is absent (unsigned build, F-Droid), the release APK is
// produced without a signingConfig — F-Droid re-signs it with its own key.
// ---------------------------------------------------------------------------
val signingJks    = file("signing.jks")
val storePass     = System.getenv("STORE_PASSWORD")     ?: ""
val keyAliasVal   = System.getenv("KEY_ALIAS")          ?: ""
val keyPassVal    = System.getenv("KEY_PASSWORD")       ?: ""
val hasReleaseKey = signingJks.exists() &&
        storePass.isNotBlank() && keyAliasVal.isNotBlank() && keyPassVal.isNotBlank()

android {
    namespace  = "com.freenet.vpn"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.freenet.vpn"
        minSdk    = 26       // Android 8.0 — minimum for reliable VpnService
        targetSdk = 35
        versionCode = 196
        versionName = "1.9.6"

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    signingConfigs {
        if (hasReleaseKey) {
            create("release") {
                storeFile     = signingJks
                storePassword = storePass
                keyAlias      = keyAliasVal
                keyPassword   = keyPassVal
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
            if (hasReleaseKey) {
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
