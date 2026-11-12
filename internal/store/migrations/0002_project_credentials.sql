CREATE TABLE project_credentials (
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    uid INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential TEXT NOT NULL UNIQUE,
    PRIMARY KEY (project_id, uid)
);
