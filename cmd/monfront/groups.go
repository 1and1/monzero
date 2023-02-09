package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

type (
	group struct {
		GroupId   int
		Name      string
		NodeId    int
		NodeName  string
		State     int
		MappingId int
	}
)

func showGroups(con *Context) {
	query := `select
        group_id,
        group_name,
        node_id,
        node_name,
        mapping_id,
        state
from (
        select
                g.id group_id,
                g.name group_name,
                n.id node_id,
                n.name node_name,
                ac.states[1] state,
                ac.mapping_id,
                ac.acknowledged,
                row_number() over (partition by c.node_id order by ac.states[1] desc) maxstate
        from groups g
        join nodes_groups ng on g.id = ng.group_id
        join nodes n on ng.node_id = n.id
        join checks c on n.id = c.node_id
        join active_checks ac on c.id = ac.check_id
				%s
        order by g.name, n.name
) groups
where maxstate = 1`
	if strings.HasPrefix(con.r.URL.Path, "/unhandled") {
		query = fmt.Sprintf(query, `where ac.states[1] != 0 and acknowledged = false`)
		con.Unhandled = true
	} else {
		query = fmt.Sprintf(query, "")
	}

	rows, err := DB.Query(query)
	if err != nil {
		con.w.WriteHeader(http.StatusInternalServerError)
		con.w.Write([]byte("problems with the database"))
		log.Printf("could not get check list: %s", err)
		return
	}

	groups := []group{}
	for rows.Next() {
		g := group{}
		err := rows.Scan(&g.GroupId, &g.Name, &g.NodeId, &g.NodeName, &g.MappingId, &g.State)
		if err != nil {
			con.w.WriteHeader(http.StatusInternalServerError)
			con.w.Write([]byte("problems with the database"))
			log.Printf("could not get check list: %s", err)
			return
		}
		groups = append(groups, g)
	}
	con.Groups = groups
	if err := con.loadMappings(); err != nil {
		con.w.WriteHeader(http.StatusInternalServerError)
		con.w.Write([]byte("problem with the mappings"))
		log.Printf("could not load mappings: %s", err)
		return
	}
	con.w.Header()["Content-Type"] = []string{"text/html"}
	con.Render("grouplist")
	return
}
