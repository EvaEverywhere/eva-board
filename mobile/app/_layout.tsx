import "../global.css";

import { useEffect } from "react";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { useFonts } from "expo-font";
import { useColorScheme } from "nativewind";
import { Inter_500Medium, Inter_700Bold } from "@expo-google-fonts/inter";
import { SpaceGrotesk_700Bold } from "@expo-google-fonts/space-grotesk";

import { AuthSessionProvider } from "@/providers/AuthSessionProvider";

export default function RootLayout() {
  const { setColorScheme } = useColorScheme();
  const [fontsLoaded] = useFonts({
    Inter_500Medium,
    Inter_700Bold,
    SpaceGrotesk_700Bold
  });

  useEffect(() => {
    setColorScheme("dark");
  }, [setColorScheme]);

  if (!fontsLoaded) {
    return null;
  }

  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <AuthSessionProvider>
        <StatusBar style="light" />
        <Stack screenOptions={{ headerShown: false }} />
      </AuthSessionProvider>
    </GestureHandlerRootView>
  );
}
