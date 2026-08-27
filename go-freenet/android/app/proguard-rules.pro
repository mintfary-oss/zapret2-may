# ProGuard rules for FreeNet VPN app.

# Keep all classes in the app package (VpnService, BootReceiver, etc.)
-keep class com.freenet.vpn.** { *; }

# Keep the gomobile-generated bindings — they use reflection.
# gomobile bind -javapkg com.freenet.bypass ./mobile generates classes in
# com.freenet.bypass.mobile (Go pkg name appended to -javapkg prefix).
-keep class com.freenet.bypass.** { *; }
-keep class com.freenet.bypass.mobile.** { *; }
-keep class go.** { *; }

# Suppress warnings for gomobile JNI classes.
-dontwarn com.freenet.bypass.**
-dontwarn com.freenet.bypass.mobile.**
-dontwarn go.**
