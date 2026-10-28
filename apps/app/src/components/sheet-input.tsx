import { createContext, useContext, type ReactElement } from 'react';
import { TextInput, type TextInputProps } from 'react-native';

type RenderInput = (props: TextInputProps) => ReactElement;

/**
 * Draws a text input: a native bottom sheet provides its own
 * (BottomSheetTextInput), which lifts the sheet above the keyboard.
 */
export const SheetTextInputContext = createContext<RenderInput>((props) => (
  <TextInput {...props} />
));

export function useSheetTextInput(): RenderInput {
  return useContext(SheetTextInputContext);
}
