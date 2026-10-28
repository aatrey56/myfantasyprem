import { Link } from 'expo-router';

import { AppText } from '@/components/AppText';
import { Screen } from '@/components/Screen';

export default function NotFound() {
  return (
    <Screen title="Page not found">
      <Link href="/">
        <AppText accent>Go home</AppText>
      </Link>
    </Screen>
  );
}
