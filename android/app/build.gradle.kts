plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "io.qorechain.lightnode"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.qorechain.lightnode"
        minSdk = 26
        targetSdk = 35
        versionCode = 3111
        versionName = "3.1.11"
        // The AAR carries one ABI. arm64 covers every current Android phone;
        // add further targets to build-aar.sh before widening this.
        ndk { abiFilters += "arm64-v8a" }
    }

    // Signed with the same upload key as QoreX Android (owner's decision,
    // 2026-10-16). Credentials are supplied out-of-band, exactly as QoreX does:
    // QOREX_UPLOAD_* in ~/.gradle/gradle.properties or -P. Never committed.
    //
    // One deliberate difference from QoreX: without the key a release build is
    // left UNSIGNED (app-release-unsigned.apk) rather than falling back to the
    // debug key, so a debug-signed APK can never be mistaken for a release and
    // published.
    val uploadKey = providers.gradleProperty("QOREX_UPLOAD_STORE_FILE")
    signingConfigs {
        if (uploadKey.isPresent) {
            create("release") {
                storeFile = file(uploadKey.get())
                storePassword = providers.gradleProperty("QOREX_UPLOAD_STORE_PASSWORD").get()
                keyAlias = providers.gradleProperty("QOREX_UPLOAD_KEY_ALIAS").get()
                keyPassword = providers.gradleProperty("QOREX_UPLOAD_KEY_PASSWORD").get()
            }
        }
    }

    buildTypes {
        release {
            // Off on purpose: R8 would strip the gomobile bridge (go.Seq and the
            // generated mobile.* classes are reached through JNI, not Kotlin).
            isMinifyEnabled = false
            if (uploadKey.isPresent) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlin {
        jvmToolchain(17)
    }
}

dependencies {
    // The light node itself: the same Go code the server and desktop builds
    // run, bound to an AAR by ../build-aar.sh. Not a reimplementation.
    implementation(files("../libs/qorelightnode.aar"))

    implementation("androidx.core:core-ktx:1.17.0")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("androidx.lifecycle:lifecycle-service:2.8.7")
}
