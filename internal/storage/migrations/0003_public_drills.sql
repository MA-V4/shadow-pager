/* A public drill is one that a visitor started without the admin token, and it must be remembered across restarts. */
ALTER TABLE simulations ADD COLUMN public boolean NOT NULL DEFAULT false;

/* The incident keeps its own copy, so Slack stays silent for it even when its simulation row is saved later. */
ALTER TABLE incidents ADD COLUMN public boolean NOT NULL DEFAULT false;
