import { createContext, useContext } from 'react';

/** True inside the league shell while the bottom tab bar is showing (it owns the bottom inset). */
export const BottomTabBarContext = createContext(false);

export const useHasBottomTabBar = () => useContext(BottomTabBarContext);

/** True inside the league shell while the phone top bar is showing (it owns the top inset). */
export const TopBarContext = createContext(false);

export const useHasTopBar = () => useContext(TopBarContext);
