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
        versionCode = 3106
        versionName = "3.1.6"
        // The AAR carries one ABI. arm64 covers every current Android phone;
        // add further targets to build-aar.sh before widening this.
        ndk { abiFilters += "arm64-v8a" }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
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
