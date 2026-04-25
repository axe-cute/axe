export type Series = {
  id: string;
  title: string;
  description: string;
  genre: string;
  author: string;
  cover_url: string;
  status: "ongoing" | "completed" | "hiatus" | string;
  /** Present only on responses from /serieses/trending. 0 otherwise. */
  trending_score?: number;
  created_at: string;
  updated_at: string;
};

export type Episode = {
  id: string;
  title: string;
  episode_number: number;
  thumbnail_url: string;
  published: boolean;
  series_id?: string;
  created_at: string;
  updated_at: string;
};

export type Bookmark = {
  id: string;
  user_id?: string;
  series_id: string;
  created_at: string;
  updated_at: string;
};

export type ListEnvelope<T> = { data: T[]; total: number };

export type LoginResponse = {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  user_id: string;
  email: string;
  role: string;
};

export type ToggleResult = { bookmarked: boolean; series_id: string };

export type ApiError = { code: string; message: string };
