-- Where the worker fetches the demo from. Empty until something resolves it:
-- today the user supplies it, later the Game Coordinator sidecar will.
ALTER TABLE demo_analyses ADD COLUMN demo_url TEXT NOT NULL DEFAULT '';
