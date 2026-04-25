export const GENRES = [
  "action",
  "romance",
  "comedy",
  "drama",
  "fantasy",
  "horror",
  "thriller",
  "slice-of-life",
  "sci-fi",
  "sports",
  "historical",
] as const;

export type Genre = (typeof GENRES)[number];

export function genreLabel(g: string): string {
  return g
    .split("-")
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}
