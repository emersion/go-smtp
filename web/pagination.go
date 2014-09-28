package web

import (
	"fmt"
	"html/template"
	"math"
	"strconv"
)

type Pagination struct {
	total	int
	page 	int
	limit	int
	url     string
	style	int

	offset   int
	pages    int
	beginnum int
	endnum   int
}

func NewPagination(total int, limit int, curpage int, url string) *Pagination {
	p := Pagination{}
	p.total = total
	p.limit = limit
	p.url   = url
	p.style = 2
	p.page  = curpage

	p.Paginate()
	return &p
}

func (p *Pagination) Paginate() {

	//Set number of records per page
	if p.limit < 0 || p.limit < 1 {
		p.limit = 10 //If there is no set, the default display 10 records per page
	}

	//Calculate the total number of pages
	p.pages = int(math.Ceil(float64(p.total) / float64(p.limit)))

	//Judgment Page settings, otherwise it is set to the first page
	if p.page < 0 || p.page < 1 {
		p.page = 1
	}
	if p.page > p.pages {
		p.page = p.pages
	}

	p.beginnum = p.page - 4
	p.endnum = p.page + 5

	if p.page < 5 {
		//The number of links available , it is now two pages before and after the current page plus total five,
		// if the conditions for half the number of available links
		p.beginnum = 1
		p.endnum = 10
	}
	if p.page > p.pages-5 {
		p.beginnum = p.pages - 9
		p.endnum = p.pages
	}
	if p.beginnum < 1 {
		p.beginnum = 1
	}
	if p.endnum > p.pages {
		p.endnum = p.pages
	}

	//Offset calculation record
	p.offset   = int((p.page - 1) * p.limit)
}

func (p *Pagination) Html(style int) (output template.HTML) {
	if p.pages <= 1 {
		return template.HTML("")
	}

	var raw string
	p.style = style

	switch {
	case p.style == 1:
		if p.total > 0 {
			raw = `<ul class="pager">`
			if p.page > 1 {
				raw += fmt.Sprintf(`<li class="previous"><a href="%s/%s">Previous</a></li>`, p.url, strconv.Itoa(p.page-1))
			}

			raw += fmt.Sprintf(`<li class="number">%d/%d</li>`, p.page, p.pages)

			if p.page < p.pages {
				raw += fmt.Sprintf(`<li class="next"><a href="%s/%s">Next</a></li>`, p.url, strconv.Itoa(p.page+1))
			}
		}

		output = template.HTML(raw)
	case style == 2:
		if p.total > 0 {
			raw = "<ul class='pagination'>"
			count := p.pages + 1
			//begin page
			if (p.page != p.beginnum) && (p.page > p.beginnum) {
				raw = raw + "<li><a href='" + p.url + "/" + strconv.Itoa(p.page-1) + "'>&laquo;</a></li>"
			}
			for i := 1; i < count; i++ {
				//current page and loop pages
				if ((i > p.beginnum-2) && (p.page > p.beginnum)) && ((i < p.endnum+1) && (p.page < p.endnum)) {
					if i == p.page {
						raw = raw + "<li class='active'><a href='#'>" + strconv.Itoa(i) + "</a></li>"
					} else {
						raw = raw + "<li><a href='" + p.url + "/" + strconv.Itoa(i) + "'>" + strconv.Itoa(i) + "</a></li>"
					}
				}
				//next page
				if (p.page != p.endnum) && (p.page < p.endnum) && (i == p.pages) {
					raw = raw + "<li><a href='" + p.url + "/" + strconv.Itoa(p.page+1) + "'>&raquo;</a></li>"
				}
			}
			raw = raw + "</ul>"
		}

		output = template.HTML(raw)
	case p.style == 3:
		if p.total > 0 {
			raw = ""
			//begin page
			if (p.page != p.beginnum) && (p.page > p.beginnum) {
				raw += "<a href='" + p.url + "/" + strconv.Itoa(p.page-1) + "' class='btn btn-default'><span class='glyphicon glyphicon-chevron-left'></span></a>"
			} else if p.page == p.beginnum {
				raw += "<button type='button' class='btn btn-default' disabled='disabled'><span class='glyphicon glyphicon-chevron-left'></span></button>"
			}

			//last page
			if (p.page != p.endnum) && (p.page < p.endnum){
				raw += "<a href='" + p.url + "/" + strconv.Itoa(p.page+1) + "' class='btn btn-default'><span class='glyphicon glyphicon-chevron-right'></span></a>"
			} else if p.page == p.endnum {
				raw += "<button type='button' class='btn btn-default' disabled='disabled'><span class='glyphicon glyphicon-chevron-right'></span></button>"
			}
		}

		output = template.HTML(raw)
	}

	return output
}

func (p *Pagination) Total() int {
	return p.total
}

func (p *Pagination) Offset() int {
	return p.offset
}

func (p *Pagination) Limit() int {
	return p.limit
}

func (p *Pagination) Pages() int {
	return p.pages
}
