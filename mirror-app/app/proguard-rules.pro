# zxing-embedded 依赖 zxing core，保持注解与枚举，防止 release 混淆误删
-keep class com.google.zxing.** { *; }
-keep class com.journeyapps.barcodescanner.** { *; }
