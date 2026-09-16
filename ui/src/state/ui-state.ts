import { create } from "zustand";

type UIState = {
  sidebarOpen: boolean;
  setSidebarOpen: (open: boolean) => void;
};

export const useUIState = create<UIState>((set) => ({
  sidebarOpen: true,
  setSidebarOpen: (sidebarOpen) => set({ sidebarOpen }),
}));
