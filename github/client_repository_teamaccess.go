/*
Copyright 2020 The Flux CD contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package github

import (
	"context"
	"errors"

	"github.com/fluxcd/go-git-providers/gitprovider"
)

// TeamAccessClient implements the gitprovider.TeamAccessClient interface.
var _ gitprovider.TeamAccessClient = &TeamAccessClient{}

// TeamAccessClient operates on the teams list for a specific repository.
type TeamAccessClient struct {
	*clientContext
	ref gitprovider.RepositoryRef
}

// Get a team within the specific organization.
//
// name may include slashes, but must not be an empty string.
// Teams are sub-groups in GitLab.
//
// ErrNotFound is returned if the resource does not exist.
//
// TeamAccess.APIObject will be nil, because there's no underlying Github struct.
func (c *TeamAccessClient) Get(ctx context.Context, name string) (gitprovider.TeamAccess, error) {
	// GET /orgs/{org}/teams/{team_slug}/repos/{owner}/{repo}
	permissionMap, err := c.c.GetTeamPermissions(ctx, c.ref.GetIdentity(), c.ref.GetRepository(), name)
	if err != nil {
		return nil, err
	}

	return newTeamAccess(c, gitprovider.TeamAccessInfo{
		Name:       name,
		Permission: getPermissionFromMap(permissionMap),
	}), nil
}

// List lists the team access control list for this repository.
//
// List returns all available team access lists, using multiple paginated requests if needed.
func (c *TeamAccessClient) List(ctx context.Context) ([]gitprovider.TeamAccess, error) {
	// List all teams, using pagination. This does not contain information about the members
	apiObjs, err := c.c.ListRepoTeams(ctx, c.ref.GetIdentity(), c.ref.GetRepository())
	if err != nil {
		return nil, err
	}

	teamAccess := make([]gitprovider.TeamAccess, 0, len(apiObjs))
	for _, apiObj := range apiObjs {
		// Get more detailed info about the team, we know that Slug is non-nil as of ListTeams.
		ta, err := c.Get(ctx, *apiObj.Slug)
		if err != nil {
			return nil, err
		}
		teamAccess = append(teamAccess, ta)
	}

	return teamAccess, nil
}

// Create adds a given team to the repo's team access control list.
//
// ErrAlreadyExists will be returned if the resource already exists.
func (c *TeamAccessClient) Create(ctx context.Context, req gitprovider.TeamAccessInfo) (gitprovider.TeamAccess, error) {
	// First thing, validate and default the request to ensure a valid and fully-populated object
	// (to minimize any possible diffs between desired and actual state)
	if err := gitprovider.ValidateAndDefaultInfo(&req); err != nil {
		return nil, err
	}

	// PUT /orgs/{org}/teams/{team_slug}/repos/{owner}/{repo}
	if err := c.c.AddTeam(ctx, c.ref.GetIdentity(), c.ref.GetRepository(), req.Name, *req.Permission); err != nil {
		return nil, err
	}

	return newTeamAccess(c, req), nil
}

// Reconcile makes sure the given desired state (req) becomes the actual state in the backing Git provider.
//
// If req doesn't exist under the hood, it is created (actionTaken == true).
// If req doesn't equal the actual state, the resource will be deleted and recreated (actionTaken == true).
// If req is already the actual state, this is a no-op (actionTaken == false).
func (c *TeamAccessClient) Reconcile(ctx context.Context,
	req gitprovider.TeamAccessInfo,
) (gitprovider.TeamAccess, bool, error) {
	// First thing, validate and default the request to ensure a valid and fully-populated object
	// (to minimize any possible diffs between desired and actual state)
	if err := gitprovider.ValidateAndDefaultInfo(&req); err != nil {
		return nil, false, err
	}

	actual, err := c.Get(ctx, req.Name)
	if err != nil {
		// Create if not found
		if errors.Is(err, gitprovider.ErrNotFound) {
			resp, err := c.Create(ctx, req)
			return resp, true, err
		}

		// Unexpected path, Get should succeed or return NotFound
		return nil, false, err
	}

	// If the desired matches the actual state, just return the actual state
	if req.Equals(actual.Get()) {
		return actual, false, nil
	}

	// Populate the desired state to the current-actual object
	if err := actual.Set(req); err != nil {
		return actual, false, err
	}
	return actual, true, actual.Update(ctx)
}
