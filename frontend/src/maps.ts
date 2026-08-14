/**
 * Display names for CS2 maps.
 *
 * Demos carry the internal name (de_mirage), which is what the game uses
 * everywhere except in front of players. The list is deliberately not
 * exhaustive — community and workshop maps exist without limit — so anything
 * unknown falls back to a sensible transformation rather than being dropped.
 */
const MAP_NAMES: Record<string, string> = {
  // Active duty and reserve
  de_ancient: "Ancient",
  de_anubis: "Anubis",
  de_dust2: "Dust II",
  de_inferno: "Inferno",
  de_mirage: "Mirage",
  de_nuke: "Nuke",
  de_overpass: "Overpass",
  de_train: "Train",
  de_vertigo: "Vertigo",
  de_cache: "Cache",
  de_cbble: "Cobblestone",

  // Community maps that have appeared in the map pool
  de_thera: "Thera",
  de_mills: "Mills",
  de_grail: "Grail",
  de_basalt: "Basalt",
  de_edin: "Edin",
  de_jura: "Jura",
  de_memento: "Memento",
  de_assembly: "Assembly",
  de_dogtown: "Dogtown",
  de_palais: "Palais",
  de_whistle: "Whistle",

  // Hostage and older classics
  cs_office: "Office",
  cs_italy: "Italy",
  cs_agency: "Agency",

  // Wingman / arms race
  de_lake: "Lake",
  de_safehouse: "Safehouse",
  de_shortdust: "Short Dust",
  de_shortnuke: "Short Nuke",
  ar_baggage: "Baggage",
  ar_shoots: "Shoots",
  ar_pool_day: "Pool Day",
};

/**
 * Turns an internal map name into something readable.
 *
 * Unknown maps drop the game-mode prefix and title-case the rest, so a
 * workshop map called de_someplace reads as "Someplace" rather than being
 * shown raw or hidden.
 */
export function mapDisplayName(internal: string | undefined | null): string {
  if (!internal) return "";

  const known = MAP_NAMES[internal.toLowerCase()];
  if (known) return known;

  return internal
    .replace(/^(de|cs|ar|dz|gd|coop|training)_/i, "")
    .split(/[_\s]+/)
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}
