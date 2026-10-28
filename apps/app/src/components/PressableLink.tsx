import { useRouter, type Href } from 'expo-router';
import { Pressable, type PressableProps } from 'react-native';

interface Props extends Omit<PressableProps, 'onPress'> {
  href: Href | string;
  /** `navigate` returns to the route if it is already in the stack. Default `push`. */
  mode?: 'push' | 'replace' | 'navigate';
}

/**
 * A pressable that navigates, announced as a link. Used instead of
 * `<Link asChild>` because Link's slot drops Pressable's style function
 * (hover, press and focus feedback) on web.
 */
export function PressableLink({ href, mode = 'push', ...rest }: Props) {
  const router = useRouter();
  return (
    <Pressable accessibilityRole="link" {...rest} onPress={() => router[mode](href as Href)} />
  );
}
