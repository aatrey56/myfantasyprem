import { Redirect } from 'expo-router';
import { View } from 'react-native';

import { useAuth } from '@/auth/AuthProvider';
import { useTheme } from '@/theme';

/** Signed in: the leagues hub. Signed out: sign in. */
export default function Index() {
  const { status } = useAuth();
  const { colors } = useTheme();
  if (status === 'loading') return <View style={{ flex: 1, backgroundColor: colors.background }} />;
  return <Redirect href={status === 'signedIn' ? '/leagues' : '/sign-in'} />;
}
