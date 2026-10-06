import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.android.application) // AGP 9 applies Kotlin itself (built-in Kotlin)
    alias(libs.plugins.compose.compiler)    // Compose compiler, same version as Kotlin (2.4.20)
    alias(libs.plugins.jetbrains.compose)   // JetBrains Compose runtime/DSL (matches miuix)
}

android {
    namespace = "com.vmpacker.app"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.vmpacker.app"
        minSdk = 24          // must be >= the gomobile -androidapi used for the .aar (24)
        targetSdk = 36
        versionCode = 1
        versionName = "1.0"

        // The gomobile .aar ships native .so only for these ABIs.
        ndk {
            abiFilters += listOf("arm64-v8a", "armeabi-v7a")
        }
    }

    buildTypes {
        debug {
            isMinifyEnabled = false
        }
        release {
            // Keep release un-minified for now; the gomobile .aar bundles its own
            // consumer proguard rules (keeps go.** and the bound package).
            isMinifyEnabled = false
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
    }

    // The source set uses kotlin/ instead of java/.
    sourceSets["main"].java.srcDirs("src/main/kotlin")

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}

dependencies {
    // Compose runtime/UI come from the JetBrains Compose plugin so their ABI
    // matches what miuix-ui:0.9.4 was compiled against.
    implementation(compose.runtime)
    implementation(compose.foundation)
    implementation(compose.ui)

    implementation(libs.androidx.activity.compose)
    implementation(libs.kotlinx.coroutines.android)

    // The MiUIX-style component library.
    implementation(libs.miuix.ui)

    // The gomobile-generated binding to VMPacker's Go engine.
    // Produced by CI (gomobile bind -> android/app/libs/vmpmobile.aar).
    implementation(files("libs/vmpmobile.aar"))
}
