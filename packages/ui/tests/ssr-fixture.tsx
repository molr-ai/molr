import { UIProvider, ToastProvider, Logo, DateTimePicker, RangeSlider, useBreakpoint, useTheme, useToast, useModalScope, getOverlayViewport, composeRefs } from '../src/index';
export { getLogoPlayback, getLogoTimeline } from '../src/logo-playback';
export function ServerFixture() {
  return <UIProvider brandAssetBase="/nested/brand"><ToastProvider><Content /></ToastProvider></UIProvider>;
}
function Content() {
  const theme = useTheme();
  const mobile = useBreakpoint('mobile');
  const { toast } = useToast();
  const scope = useModalScope();
  const viewport = getOverlayViewport();
  void toast; void scope; void composeRefs;
  return <><output data-mobile={mobile}>{theme.preference}:{theme.theme}:{viewport.width}</output><Logo animation="alive" interactive /><RangeSlider label="Fixed range" value={[5, 5]} min={5} max={5} onValueChange={() => {}} /><DateTimePicker label="Expiry" value={null} onValueChange={() => {}} /></>;
}
