import { render, screen } from '@testing-library/react-native';
import { Platform, StyleSheet } from 'react-native';

import { TextField } from './TextField';

describe('TextField multiline', () => {
  it('lets a native multiline field grow: no numberOfLines cap off the web', async () => {
    expect(Platform.OS).not.toBe('web');
    await render(<TextField label="Message" multiline testID="msg" />);
    expect(screen.getByTestId('msg').props.numberOfLines).toBeUndefined();
  });

  it('pads a multiline field evenly so one line sits centred', async () => {
    await render(<TextField label="Message" multiline testID="msg" />);
    const style = StyleSheet.flatten(screen.getByTestId('msg').props.style);
    expect(style.paddingVertical).toBeGreaterThan(0);
    expect(style.lineHeight).toBeGreaterThan(0);
  });

  it('leaves a single-line field as it was', async () => {
    await render(<TextField label="Name" testID="name" />);
    const style = StyleSheet.flatten(screen.getByTestId('name').props.style);
    expect(style.lineHeight).toBeUndefined();
  });
});
