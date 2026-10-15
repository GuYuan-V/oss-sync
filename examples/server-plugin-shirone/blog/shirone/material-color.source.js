import {
  Hct, SchemeContent, SchemeExpressive, SchemeFidelity, SchemeFruitSalad,
  SchemeMonochrome, SchemeNeutral, SchemeRainbow, SchemeTonalSpot, SchemeVibrant,
} from '@material/material-color-utilities';

const schemes = {
  tonalSpot: SchemeTonalSpot, vibrant: SchemeVibrant, content: SchemeContent,
  expressive: SchemeExpressive, rainbow: SchemeRainbow, fruitSalad: SchemeFruitSalad,
  monochrome: SchemeMonochrome, neutral: SchemeNeutral, fidelity: SchemeFidelity,
};
const roles = [
  'primary', 'onPrimary', 'primaryContainer', 'onPrimaryContainer', 'inversePrimary',
  'primaryFixed', 'primaryFixedDim', 'onPrimaryFixed', 'onPrimaryFixedVariant',
  'secondary', 'onSecondary', 'secondaryContainer', 'onSecondaryContainer',
  'secondaryFixed', 'secondaryFixedDim', 'onSecondaryFixed', 'onSecondaryFixedVariant',
  'tertiary', 'onTertiary', 'tertiaryContainer', 'onTertiaryContainer',
  'tertiaryFixed', 'tertiaryFixedDim', 'onTertiaryFixed', 'onTertiaryFixedVariant',
  'error', 'onError', 'errorContainer', 'onErrorContainer', 'surface', 'surfaceDim',
  'surfaceBright', 'surfaceContainerLowest', 'surfaceContainerLow', 'surfaceContainer',
  'surfaceContainerHigh', 'surfaceContainerHighest', 'onSurface', 'surfaceVariant',
  'onSurfaceVariant', 'outline', 'outlineVariant', 'inverseSurface', 'inverseOnSurface',
  'shadow', 'scrim', 'surfaceTint', 'primaryDim', 'secondaryDim', 'tertiaryDim', 'errorDim',
];

export function resolveScheme(hue, isDark, style, spec) {
  const seed = Hct.from(Number.isFinite(hue) ? hue : 315, 60, 50);
  const Scheme = Object.hasOwn(schemes, style) ? schemes[style] : SchemeTonalSpot;
  const scheme = new Scheme(Hct.fromInt(seed.toInt()), isDark, 0, spec === '2021' ? '2021' : '2025');
  return Object.fromEntries(roles.map((role) => {
    const color = scheme.colors[role]();
    const value = color ? `#${(color.getArgb(scheme) & 0xffffff).toString(16).padStart(6, '0')}` : null;
    return [role, value];
  }));
}
