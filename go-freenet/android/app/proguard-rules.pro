# ProGuard rules for FreeNet VPN app.

# Keep all classes in the app package (VpnService, BootReceiver, etc.)
-keep class com.freenet.vpn.** { *; }

# Keep the gomobile-generated bindings — they use reflection.
-keep class com.freenet.bypass.** { *; }
-keep class go.** { *; }

# Suppress warnings for gomobile JNI classes.
-dontwarn com.freenet.bypass.**
-dontwarn go.**
