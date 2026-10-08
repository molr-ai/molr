// Keep the bend handles parallel to the adjoining lines in every pose.
// The fixed ratio also preserves tangency throughout SVG path interpolation.
export function headContour({
  leftEar = [10, 12], rightEar = [52, 12],
  leftBend = [24, 22], rightBend = [38, 22],
  leftFoot = [8.75, 41], rightFoot = [53.25, 41],
} = {}) {
  const handle = (bend, ear) => bend.map((value, i) => value + (value - ear[i]) / 3);
  const point = values => values.map(value => Number(value.toFixed(6))).join(' ');
  return `M${point(leftFoot)}L${point(leftEar)}L${point(leftBend)}C${point(handle(leftBend, leftEar))} ${point(handle(rightBend, rightEar))} ${point(rightBend)}L${point(rightEar)}L${point(rightFoot)}`;
}

export function tailContour({ bendX = 167, tip = [169, 12], top = 21, stem = 31 } = {}) {
  // The stem joins the first curve vertically; both sides of the horizontal
  // segment stay horizontal even when the tip rises, droops or curls inward.
  return `M134 41V${stem}C134 ${stem - 7} 141 ${top} 151 ${top}H157Q${bendX} ${top} ${tip.join(' ')}`;
}
