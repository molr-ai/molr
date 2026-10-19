export type RuntimeEvent = {
  Topic: string; Object?: { ID: string }; ConversationID?: string; Version?: number;
};
export type NotificationPreferences = { enabled: boolean; preview: boolean; sound: boolean; system: boolean };
export type NotificationCapability = 'granted' | 'default' | 'denied' | 'insecure' | 'unsupported';
export type MessageNotification = { tag: string; title: string; body: string; route: string; silent?: boolean };
/** A desktop host can supply a native adapter without changing message policy. */
export interface NotificationAdapter {
  capability(): NotificationCapability;
  requestPermission(): Promise<NotificationCapability>;
  show(notification: MessageNotification): Promise<void>;
  close(tag?: string): Promise<void>;
  dispose(): void;
}
