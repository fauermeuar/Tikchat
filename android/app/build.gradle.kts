plugins {
    id("com.android.application")
    kotlin("android")
}

android {
    namespace = "com.tikchat.app"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.tikchat.app"
        minSdk = 26
        targetSdk = 34
        versionCode = 149
        versionName = "1.4.9"
    }

    buildTypes {
        getByName("debug") {
            applicationIdSuffix = ".debug"
        }
        getByName("release") {
            isMinifyEnabled = false
            isShrinkResources = false
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
}

// Tanpa dependency eksternal: cukup framework Android + Kotlin stdlib.
// (Sengaja tanpa AndroidX agar build ringan & cepat.)
dependencies {
}
