// AGP 9.0+ ships built-in Kotlin support (its default KGP is older than what
// miuix 0.9.4 is built against). Force the Kotlin Gradle Plugin to 2.4.20 on the
// build classpath — the officially documented way to raise AGP's built-in Kotlin
// version — so the Compose compiler matches miuix's ABI.
// See https://developer.android.com/build/releases/agp-9-0-0-release-notes
buildscript {
    dependencies {
        classpath("org.jetbrains.kotlin:kotlin-gradle-plugin:2.4.20")
    }
}

plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.jetbrains.compose) apply false
}
