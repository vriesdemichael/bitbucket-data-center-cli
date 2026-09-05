package repository

// TestListRepositoriesAuthError is live now, in
// TestLiveErrorTaxonomyRejectedCredentials: a token that is not a token,
// refused by a real instance, and the exit code that refusal maps to. The
// unit version answered 401 to every request, which asserted that our mapper
// maps a 401 we wrote ourselves.
